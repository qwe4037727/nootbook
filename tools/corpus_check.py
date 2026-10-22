import argparse
import json
import os
import re
import sys
import zipfile
import xml.etree.ElementTree as ET

DEFAULT_SRC = r"D:\go\corpus\无边悔恨.txt"
DEFAULT_TOC = r"D:\go\.tmp_fq\toc.json"
CHARSET = r"D:\go\.tmp_fq\charset.json"
CODE = ((58344, 58715), (58345, 58716))
MIN_CHARS = 150000
PREVIEW_MARKERS = ("扫码下载", "SVIP", "会员登录后", "可在网页畅读")


def read_docx(path):
    with zipfile.ZipFile(path) as z:
        root = ET.fromstring(z.read("word/document.xml"))
    ns = "{http://schemas.openxmlformats.org/wordprocessingml/2006/main}"
    paras = []
    for p in root.iter(ns + "p"):
        text = "".join(t.text or "" for t in p.iter(ns + "t"))
        paras.append(text)
    return "\n".join(paras)


def read_source(path):
    if path.lower().endswith(".docx"):
        return read_docx(path)
    if path.lower().endswith(".doc"):
        raise SystemExit("旧版 .doc 无法直接解析，请另存为 .docx 或 .txt")
    raw = open(path, "rb").read()
    for enc in ("utf-8-sig", "gb18030", "utf-16"):
        try:
            return raw.decode(enc)
        except UnicodeDecodeError:
            continue
    raise SystemExit("无法识别编码（尝试过 UTF-8 / GB18030 / UTF-16）")


def decode_pua(text):
    if not os.path.exists(CHARSET):
        return text, 0, 0
    charset = json.load(open(CHARSET, encoding="utf-8"))
    before = sum(1 for c in text if 0xE000 <= ord(c) <= 0xF8FF)
    out = []
    for ch in text:
        uni = ord(ch)
        if CODE[0][0] <= uni <= CODE[0][1]:
            bias = uni - CODE[0][0]
            out.append(charset[0][bias] if 0 <= bias < len(charset[0]) and charset[0][bias] != "?" else ch)
        else:
            out.append(ch)
    text = "".join(out)
    after = sum(1 for c in text if 0xE000 <= ord(c) <= 0xF8FF)
    return text, before, after


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("src", nargs="?", default=DEFAULT_SRC)
    ap.add_argument("--toc", default=DEFAULT_TOC)
    args = ap.parse_args()

    if not os.path.exists(args.src):
        print("missing:", args.src)
        return 1

    toc = json.load(open(args.toc, encoding="utf-8"))
    expected = {item["num"]: item["title"] for item in toc}
    text = read_source(args.src).replace("\r\n", "\n").replace("\r", "\n")
    text, pua_before, pua_after = decode_pua(text)
    lines = text.split("\n")

    headings = []
    for idx, line in enumerate(lines):
        m = re.match(r"^\s*第\s*(\d+)\s*章[ \t　]*(.*?)\s*$", line)
        if m:
            headings.append((int(m.group(1)), m.group(2), idx))

    nums = [h[0] for h in headings]
    dupes = sorted({n for n in nums if nums.count(n) > 1})
    found = set(nums)
    missing = [n for n in sorted(expected) if n not in found]
    extra = sorted(found - set(expected))
    body_chars = sum(1 for c in text if not c.isspace())
    han_chars = sum(1 for c in text if "\u4e00" <= c <= "\u9fff")
    markers = {m: text.count(m) for m in PREVIEW_MARKERS if m in text}

    short = []
    for i, (num, _title, idx) in enumerate(headings):
        end = headings[i + 1][2] if i + 1 < len(headings) else len(lines)
        size = sum(1 for c in "".join(lines[idx + 1:end]) if not c.isspace())
        if size < 300:
            short.append((num, size))

    title_mismatch = []
    for num, title, _ in headings:
        exp = expected.get(num)
        if not exp:
            continue
        want = re.sub(r"\s+", "", exp.split("章", 1)[1])
        got = re.sub(r"\s+", "", title)
        if want and got and want != got:
            title_mismatch.append((num, exp, title))

    ok = (not missing and not dupes and pua_after == 0 and not markers
          and body_chars >= MIN_CHARS and not short)

    print(f"src: {args.src}")
    print(f"chapters: {len(headings)}/{len(expected)}  missing={missing}  extra={extra}  dupes={dupes}")
    print(f"chars(non-space): {body_chars}  han: {han_chars}  min_expected: {MIN_CHARS}")
    print(f"pua: before={pua_before} after={pua_after}")
    print(f"preview_markers: {markers or 'none'}")
    print(f"short_chapters(<300): {short or 'none'}")
    print(f"title_mismatch: {title_mismatch or 'none'}")
    print("RESULT:", "PASS" if ok else "INCOMPLETE")
    return 0 if ok else 2


if __name__ == "__main__":
    sys.exit(main())
