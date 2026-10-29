# Security

Directions policies and prompt text are not permission enforcement. Pi
extensions execute trusted host code and are not sandboxed. Only load reviewed,
explicitly approved extension sources and dependency locks.

The Agent action starts without general filesystem/shell tools, does not load
repository Pi resources, rejects pull_request_target, and keeps publication in
scoped plugin tools. Enabling another extension can change that capability
surface. Use least-privileged workflow tokens and do not run untrusted PR code
in a credential-bearing job.

HTTPS guidance requires exact host-owned URL approvals, verified TLS, bounded
reads, and no redirects or ambient authentication. Approving a URL grants network
access to that destination; do not derive approvals from untrusted repository or
PR content. Prefer SHA-256 pins. Remote fragments cannot introduce further reads.

Do not disclose credentials or private source in public issues. Use GitHub's
private vulnerability reporting when available; otherwise contact the repository
maintainers privately.

See agent/README.md and tools/pi/github/README.md for exact guarantees,
limitations, read-before-write races, and ambiguous-publication handling.
