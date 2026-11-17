# Build-only Picomatch security patch

This private local fork contains Picomatch 4.0.7 under its original MIT license.
It is used by Vite, tinyglobby and fdir when building the interface. It is not
included in the distributed Go server or browser bundle.

The npm release was copied from `https://registry.npmjs.org/picomatch/-/picomatch-4.0.7.tgz`,
verified against the previous lockfile’s integrity:

`sha512-qcJu88Q2IWqJsDD529JKMdwGm/dvInW4HvQnRwiH9JtihJvzGOscDtHE3x1pBKeUOTysQ8kVmLnJ2kJu7yhcGA==`. Original runtime files and copyrights are retained.
The only runtime change is the single-wildcard repetition rewrite in
`lib/parse.js`, based on [upstream proposal #254](https://github.com/micromatch/picomatch/pull/254)
at commit `c63a5bf4e61dbdb81bc41e97dc2616c30acce337`.

The patch addresses [GHSA-fc5q-39xc-vjpf](https://github.com/advisories/GHSA-fc5q-39xc-vjpf).
As of 2026-11-17, the advisory lists no patched npm release. This is a local
mitigation, not an official upstream release. Its scoped name distinguishes it
from the affected npm package; the version records its upstream base. The npm
audit remains enabled for registry dependencies. The local parser is covered by
the mandatory regression tests in `tests/glob-matcher.test.ts`.

For a single repeated extglob branch containing ordinary wildcard stars, the
outer repetition is redundant: earlier repetitions can be absorbed by a star.
Flattening it removes nested unbounded quantifiers while preserving yes/no file
matching. Alternatives, slash/globstar/nested patterns and the explicit
`maxExtglobRecursion: false` opt-out retain their existing handling. With
`capture: true`, the inner wildcard capture covers the flattened body rather
than only the last iteration; the build tools do not request captures.

Local validation: 1,983 upstream tests, 82,680 boolean matching comparisons
against npm 4.0.7, hostile inputs bounded by a child-process deadline, and a
controlled production build with all 62 output files byte-identical before and
after this parser change. No credentials or instance data were used.

When an upstream release fixes this advisory, replace the local dependency with
that release, remove the override and this directory, and keep the regression
tests. Run the dependency audit, production build and ordinary application tests.
