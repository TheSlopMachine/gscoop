# Security policy for gscoop

## Trusted-code notice

Bucket manifests are trusted code. Manifest hooks (`pre_install`,
`post_install`, `pre_uninstall`, `post_uninstall`, `installer.script`,
`uninstaller.script`, and `.ps1` installer files) run with the caller
privileges, elevated for global (`-g`) installs. Install only from buckets
you trust and review manifests before installing unknown packages.

## Supported versions

Only the latest tagged release receives security fixes. Untagged
pre-release checkouts are unsupported.

## Reporting a vulnerability

Report vulnerabilities privately through a GitHub private vulnerability
report on `TheSlopMachine/gscoop`. Include the affected version, the
manifest or command involved, and reproduction steps. Do not open a public
issue for a suspected vulnerability. Expect an initial response within
7 days.
