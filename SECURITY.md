# Security Policy

## Supported Versions
The `main` branch is actively maintained. Older tags may not receive security updates.

## Reporting a Vulnerability
Please DO NOT create a public GitHub issue for security problems.

Instead:
1. Email: security@rotur.dev (if available) or open a private advisory (GitHub Security Advisories).
2. Provide details: affected endpoints, reproduction steps, potential impact.
3. Allow up to 72 hours for initial acknowledgement.

## Guidelines
- Use only your own test data; do not exfiltrate real user data.
- Rate limiting / brute force testing should be minimal.
- Do not run automated scanners against production without permission.

## Preferred Topics
- Auth bypass / privilege escalation
- Insecure direct object references
- Injection (JSON, path traversal, etc.)
- Data corruption or unauthorized persistence writes

## Non-Issues
- Lack of rate limiting on non-sensitive endpoints (unless leading to abuse)
- Missing security headers (unless causing practical exploitability)
- The presence of public test/demo credentials (none should exist; report if you find one)

Thanks for helping keep users safe.
