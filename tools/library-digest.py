#!/usr/bin/env python3
"""Compute the library content digest the PlatformAuditGraphListener records.

This mirrors libraryDigest() in
talos-argocd-proxmox/my-apps/development/jenkins-lab/01-audit-graph-listener.groovy
EXACTLY: a SHA-256 over every *.groovy file under the library root, sorted by absolute
path, hashing for each file:

    <path relative to the library root> + 0x00 + <raw file bytes>

Use it to (re)generate the expected-digest registry that the library-integrity Cedar
gate checks against (the ConfigMap mounted at LIBRARY_DIGESTS_PATH on the coordinator).

The AUTHORITATIVE value is the `library_digests` map a real build's audit summary
reports; this script reproduces that value offline from a checkout so the registry can
be maintained in git. Because jenkins-library is loaded at defaultVersion "main" (a
moving branch), this digest changes whenever main advances -- regenerate it on every
jenkins-library change (ideally from that repo's CI) and confirm it against the first
build's library_digests before trusting it (the in-pod checkout is the source of truth).

Usage:
    library-digest.py <library-checkout-dir>
e.g.
    git clone https://github.com/pboyd-oss/jenkins-library && \
        library-digest.py ./jenkins-library
"""
import hashlib
import os
import sys


def library_digest(root: str) -> str:
    root = os.path.abspath(root).rstrip("/")
    files = []
    for dirpath, _dirs, names in os.walk(root):
        for n in names:
            if n.endswith(".groovy"):
                files.append(os.path.join(dirpath, n))
    files.sort()  # by absolute path, matching the Groovy `it.absolutePath` sort
    h = hashlib.sha256()
    for f in files:
        rel = f[len(root):]  # leading "/" preserved, matches substring(len) in Groovy
        h.update(rel.encode("utf-8"))
        h.update(b"\x00")
        with open(f, "rb") as fh:
            h.update(fh.read())
    return h.hexdigest()


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit("usage: library-digest.py <library-checkout-dir>")
    print(library_digest(sys.argv[1]))
