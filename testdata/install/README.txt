# Install transaction fixture notes
#
# Plan input: apps [demo, base] at 64bit.
# - demo depends on base (see demo-manifest.json depends).
# - Expected dependency order: base, demo.
# - Staging: apps/demo/<version>.tmp renamed to apps/demo/<version>.
# - Commit: scoop-manifest.json (manifest bytes) plus
#   scoop-install.json ordered keys architecture, bucket, url.
# - Junction: apps/demo/current flips to the new version dir.
# - Failure leaves dir present with no current and no metadata
#   (classic failed() state).
#
# Files:
# - demo-manifest.json: synthetic manifest exercising bin triples,
#   shortcuts, env_add_path, env_set, persist pairs, psmodule,
#   installer file plus args, and pre/post_install scripts.
# - uninstall-order.txt: reverse pipeline checklist.
# - persist-case.txt: persist move/link expectations.
