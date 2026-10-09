#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Samples memory every INTERVAL seconds until killed, as one line per
sample (MiB):

  time mem_avail boundedcode llama docker_vm containers cbm

boundedcode, llama and cbm are the resident sets of those processes; the
Docker VM is Docker Desktop's VM process (qemu or the Linux VM helper);
containers is the sum of `docker stats` memory use.

usage: sampler.py OUT [--interval 10]
"""
import argparse
import os
import re
import subprocess
import time

GROUPS = {
    "boundedcode": re.compile(r"(^|/)boundedcode$"),
    "llama": re.compile(r"(^|/)llama-server$"),
    "docker_vm": re.compile(r"qemu-system|virtiofsd|com\.docker\.virtualization|vpnkit"),
    "cbm": re.compile(r"(^|/)codebase-memory-mcp$"),
}


def rss_by_group():
    out = {k: 0 for k in GROUPS}
    for pid in os.listdir("/proc"):
        if not pid.isdigit():
            continue
        try:
            exe = os.readlink("/proc/%s/exe" % pid)
        except OSError:
            try:
                exe = open("/proc/%s/comm" % pid).read().strip()
            except OSError:
                continue
        for k, rx in GROUPS.items():
            if rx.search(exe):
                try:
                    for line in open("/proc/%s/status" % pid):
                        if line.startswith("VmRSS:"):
                            out[k] += int(line.split()[1]) // 1024
                except OSError:
                    pass
    return out


def mem_available():
    for line in open("/proc/meminfo"):
        if line.startswith("MemAvailable:"):
            return int(line.split()[1]) // 1024
    return 0


def containers():
    try:
        out = subprocess.run(["docker", "stats", "--no-stream", "--format", "{{.MemUsage}}"], capture_output=True, text=True, timeout=20).stdout
    except (OSError, subprocess.TimeoutExpired):
        return -1
    total = 0.0
    units = {"B": 1 / 2**20, "KiB": 1 / 1024, "MiB": 1, "GiB": 1024, "TiB": 2**20}
    for line in out.splitlines():
        m = re.match(r"\s*([\d.]+)\s*([KMGT]?i?B)", line)
        if m:
            total += float(m.group(1)) * units.get(m.group(2), 1)
    return int(total)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("out")
    ap.add_argument("--interval", type=float, default=10)
    a = ap.parse_args()
    with open(a.out, "a") as f:
        f.write("time mem_avail boundedcode llama docker_vm containers cbm\n")
        while True:
            g = rss_by_group()
            f.write("%s %d %d %d %d %d %d\n" % (time.strftime("%H:%M:%S", time.gmtime()), mem_available(), g["boundedcode"], g["llama"], g["docker_vm"], containers(), g["cbm"]))
            f.flush()
            time.sleep(a.interval)


if __name__ == "__main__":
    main()
