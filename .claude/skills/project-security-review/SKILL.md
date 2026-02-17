# Codebase Security Review

Perform a security-focused review of the entire codebase (or a specified directory) to identify HIGH-CONFIDENCE security vulnerabilities with real exploitation potential. This is not a general code review — focus ONLY on security implications.

## Arguments

- `$ARGUMENTS` — Optional path to a specific directory or module to scope the review (e.g., `x/market`). If omitted, review the full codebase.

## Steps

### Phase 1 — Scope & Inventory

1. Determine the review scope:
   - If `$ARGUMENTS` is provided, scope to that directory
   - Otherwise, identify all source directories (exclude `vendor/`, `testutil/`, `*_test.go`, `docs/`)
2. Inventory the languages, frameworks, and security-relevant libraries in use (check `go.mod`, `package.json`, etc.)
3. Identify established security patterns already in the codebase (input validation, auth checks, sanitization)

### Phase 2 — Systematic Analysis

Use sub-tasks (Task tool with `subagent_type: "Explore"`) to parallelize analysis across modules/directories. For each area, examine:

**Input Validation Vulnerabilities:**
- SQL injection via unsanitized user input
- Command injection in system calls or subprocesses
- XXE injection in XML parsing
- Template injection in templating engines
- NoSQL injection in database queries
- Path traversal in file operations

**Authentication & Authorization Issues:**
- Authentication bypass logic
- Privilege escalation paths
- Session management flaws
- JWT token vulnerabilities
- Authorization logic bypasses

**Crypto & Secrets Management:**
- Hardcoded API keys, passwords, or tokens
- Weak cryptographic algorithms or implementations
- Improper key storage or management
- Cryptographic randomness issues
- Certificate validation bypasses

**Injection & Code Execution:**
- Remote code execution via deserialization
- YAML deserialization vulnerabilities
- Eval injection in dynamic code execution
- XSS vulnerabilities (reflected, stored, DOM-based)

**Data Exposure:**
- Sensitive data logging or storage
- PII handling violations
- API endpoint data leakage
- Debug information exposure

**Blockchain / Cosmos SDK Specific:**
- Unchecked message signer vs authority
- Missing `ValidateBasic` or message validation
- Integer overflow/underflow in token math (even with `math.Int` / `math.LegacyDec`)
- Unprotected governance or admin-only endpoints
- State manipulation via crafted transactions
- Unbounded iteration over user-controlled collections
- Missing denomination validation on coin operations
- Incorrect error handling that silently succeeds

### Phase 3 — False Positive Filtering

For each finding, apply these hard exclusions — automatically discard:
1. Denial of Service (DOS) or resource exhaustion
2. Secrets stored on disk if otherwise secured
3. Rate limiting or service overload
4. Memory/CPU exhaustion
5. Lack of validation on non-security-critical fields without proven impact
6. Lack of hardening measures (flag concrete vulns, not missing best practices)
7. Theoretical race conditions without a concrete exploit path
8. Outdated third-party library versions (managed separately)
9. Memory safety in memory-safe languages (Go, Rust)
10. Findings only in test files (`*_test.go`, `testutil/`)
11. Log spoofing or unsanitized log output
12. SSRF that only controls the path (not host/protocol)
13. Regex injection or regex DOS
14. Insecure documentation (markdown files)
15. Missing audit logs

Apply these precedents:
- Environment variables and CLI flags are trusted values
- UUIDs are unguessable
- Client-side code doesn't need auth checks (server handles it)
- Only report logging vulns if they expose secrets, passwords, or PII

Assign a confidence score (1-10) to each finding. **Discard anything below 8.**

## Severity Guidelines

- **CRITICAL**: Directly exploitable with no preconditions — RCE, auth bypass, state corruption leading to fund theft
- **HIGH**: Exploitable under realistic conditions — privilege escalation, data breach, unauthorized token minting/burning
- **MEDIUM**: Requires specific conditions but significant impact — only include if obvious and concrete

## Output Format

Output findings in this format:

```
# Finding N: [Category]: `file:line`

* Severity: Critical / High / Medium
* Confidence: N/10
* Description: [What the vulnerability is and why it's exploitable]
* Exploit Scenario: [Concrete attack steps]
* Recommendation: [Specific fix with code pointers]
```

If no high-confidence findings, output:

```
# Security Review Complete

No high-confidence vulnerabilities identified in the reviewed scope.

**Scope:** [directories reviewed]
**Files analyzed:** [count]
**Categories checked:** [list]
```
