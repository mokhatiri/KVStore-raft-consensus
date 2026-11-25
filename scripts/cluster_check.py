"""Local 3-node cluster check: election, replication latency, leader failover.

Usage:
    go build -o kvstore ./app               # kvstore.exe on Windows
    python scripts/cluster_check.py ./kvstore [--trials 10] [--config cluster.json]

The nodes run in a temporary folder (state files and logs go there), so the
repository stays clean. Standard library only.

What it does:
  1. starts 3 nodes, waits for a leader
  2. 50 sequential writes: time to the POST answer, and until the value is
     readable on the leader and on all 3 nodes (commit + apply)
  3. sequential write throughput for 3 s (one client)
  4. a burst of 30 back-to-back writes, then checks every node has them
  5. kills the leader --trials times, measures time until a survivor reports
     itself leader, writes through it, restarts the killed node
  6. at the end, checks which acknowledged writes are present on every node
"""

import argparse
import json
import os
import shutil
import statistics
import subprocess
import tempfile
import time
import urllib.request

procs = {}
http = {}
acked = {}  # key -> value of every write the leader answered 200 to


def req(node, path, method="GET", body=None, timeout=1.0):
    data = json.dumps(body).encode() if body is not None else None
    r = urllib.request.Request(f"http://{http[node]}{path}", data=data, method=method,
                               headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(r, timeout=timeout) as resp:
        return resp.status, resp.read().decode()


def state(node):
    try:
        return json.loads(req(node, "/state", timeout=0.5)[1])
    except Exception:
        return None


def get(node, key):
    try:
        return json.loads(req(node, f"/kv/{key}", timeout=0.5)[1]).get("value")
    except Exception:
        return None


def put(node, key, value):
    try:
        status, _ = req(node, f"/kv/{key}", "POST", {"value": value})
    except Exception:
        return False
    if status == 200:
        acked[key] = value
        return True
    return False


def wait_leader(alive, timeout=10.0):
    t0 = time.perf_counter()
    while time.perf_counter() - t0 < timeout:
        for n in alive:
            s = state(n)
            if s and s.get("role") == "Leader":
                return n, s.get("term")
        time.sleep(0.005)
    return None, None


def pct(xs, p):
    xs = sorted(xs)
    return xs[min(len(xs) - 1, int(round(p / 100 * (len(xs) - 1))))] * 1000


def start(binary, workdir, node, logdir):
    log = open(os.path.join(logdir, f"node{node}.log"), "ab")
    # the node CLI reads stdin; keep a pipe open so it blocks instead of spinning
    procs[node] = subprocess.Popen([binary, "node", "-id", str(node), "-config", "cluster.json"],
                                   cwd=workdir, stdin=subprocess.PIPE,
                                   stdout=log, stderr=subprocess.STDOUT)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("binary")
    ap.add_argument("--config", default="cluster.json")
    ap.add_argument("--trials", type=int, default=3)
    args = ap.parse_args()

    binary = os.path.abspath(args.binary)
    with open(args.config, encoding="utf-8") as fh:
        cfg = json.load(fh)
    for n in cfg["nodes"]:
        http[n["id"]] = n["httpAddr"]
    nodes = sorted(http)

    workdir = tempfile.mkdtemp(prefix="kvstore-check-")
    shutil.copy(args.config, os.path.join(workdir, "cluster.json"))
    print(f"work dir (state + logs): {workdir}")
    try:
        t0 = time.perf_counter()
        for n in nodes:
            start(binary, workdir, n, workdir)
        leader, term = wait_leader(nodes, 20)
        if leader is None:
            print("FAIL: no leader elected within 20 s")
            return 1
        print(f"leader: node {leader} (term {term}), {time.perf_counter() - t0:.2f} s after launch")

        # 2. latency
        ack, on_leader, on_all = [], [], []
        for i in range(50):
            k, v = f"lat{i}", f"v{i}"
            t = time.perf_counter()
            if not put(leader, k, v):
                continue
            ack.append(time.perf_counter() - t)
            seen = set()
            while len(seen) < len(nodes) and time.perf_counter() - t < 3:
                for n in nodes:
                    if n not in seen and get(n, k) == v:
                        seen.add(n)
                        if n == leader:
                            on_leader.append(time.perf_counter() - t)
                time.sleep(0.001)
            if len(seen) == len(nodes):
                on_all.append(time.perf_counter() - t)
        for name, xs in (("POST answered", ack), ("visible on leader", on_leader),
                         (f"visible on all {len(nodes)}", on_all)):
            if xs:
                print(f"{name:<20} n={len(xs):<3} p50={pct(xs, 50):6.1f} ms  p99={pct(xs, 99):6.1f} ms")

        # 3. throughput
        cnt, t = 0, time.perf_counter()
        while time.perf_counter() - t < 3:
            put(leader, f"tp{cnt}", "x")
            cnt += 1
        print(f"sequential writes, 1 client: {cnt / (time.perf_counter() - t):.0f}/s")

        # 4. burst
        for i in range(30):
            put(leader, f"burst{i}", "b")
        time.sleep(1.5)
        for n in nodes:
            have = sum(1 for i in range(30) if get(n, f"burst{i}") == "b")
            print(f"burst of 30: node {n} has {have}/30")

        # 5. failover
        times = []
        for trial in range(args.trials):
            old, old_term = wait_leader(nodes, 10)
            procs[old].kill()
            procs[old].wait()
            t = time.perf_counter()
            alive = [n for n in nodes if n != old]
            new, new_term = wait_leader(alive, 10)
            dt = time.perf_counter() - t
            if new is None:
                print(f"trial {trial + 1}: FAIL no new leader within 10 s")
            else:
                times.append(dt)
                put(new, f"after{trial}", "ok")
                print(f"trial {trial + 1}: killed node {old} (term {old_term}) -> "
                      f"node {new} leads (term {new_term}) after {dt * 1000:.0f} ms")
            start(binary, workdir, old, workdir)
            time.sleep(2)
        if times:
            print(f"re-election over {len(times)} kills: median {statistics.median(times) * 1000:.0f} ms, "
                  f"min {min(times) * 1000:.0f} ms, max {max(times) * 1000:.0f} ms")

        # 6. durability of acknowledged writes
        time.sleep(2)
        for n in nodes:
            missing = [k for k, v in acked.items() if get(n, k) != v]
            print(f"node {n}: {len(acked) - len(missing)}/{len(acked)} acknowledged writes present"
                  + (f" (missing e.g. {missing[:5]})" if missing else ""))
        return 0
    finally:
        for p in procs.values():
            try:
                p.kill()
            except Exception:
                pass


if __name__ == "__main__":
    raise SystemExit(main())
