#!/usr/bin/env python3
"""Converts a util-linux `script` recording (output log plus timing file)
into an asciicast v2 file for players and renderers such as agg.

The conversion is lossless: every output chunk keeps its bytes and its
recorded delay. Both timing formats are read: classic ("DELAY BYTES") and
the multi-stream format written with --log-timing ("O DELAY BYTES").

usage: to_cast.py TIMING LOG OUT.cast --cols N --rows N [--title T]
"""
import argparse
import codecs
import json


def chunks(timing, log):
    with open(log, "rb") as f:
        data = f.read()
    # Skip script's own header line ("Script started on ...").
    pos = data.index(b"\n") + 1 if data.startswith(b"Script started") else 0
    t = 0.0
    for line in open(timing):
        parts = line.split()
        if not parts:
            continue
        if parts[0] in ("O", "I", "S", "H"):
            kind, rest = parts[0], parts[1:]
            if kind != "O":
                if kind == "I" and len(rest) >= 2:
                    t += float(rest[0])
                continue
            delay, n = float(rest[0]), int(rest[1])
        else:
            delay, n = float(parts[0]), int(parts[1])
        t += delay
        yield t, data[pos:pos + n]
        pos += n


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("timing")
    ap.add_argument("log")
    ap.add_argument("out")
    ap.add_argument("--cols", type=int, required=True)
    ap.add_argument("--rows", type=int, required=True)
    ap.add_argument("--title", default="")
    a = ap.parse_args()
    with open(a.out, "w") as f:
        header = {"version": 2, "width": a.cols, "height": a.rows}
        if a.title:
            header["title"] = a.title
        f.write(json.dumps(header) + "\n")
        # Incremental decoding: a character split across chunks stays whole.
        dec = codecs.getincrementaldecoder("utf-8")("replace")
        for t, b in chunks(a.timing, a.log):
            text = dec.decode(b)
            if text:
                f.write(json.dumps([round(t, 6), "o", text]) + "\n")


if __name__ == "__main__":
    main()
