# Hook preamble fixture notes
#
# Preamble assigns $dir, $original_dir, $persist_dir, $version,
# $architecture, $global (bool), $SCOOP, $SCOOP_GLOBAL from
# GSCOOP_HOOK_* environment entries. Values never interpolate into
# the script text. Invocation is powershell.exe -NoProfile
# -NonInteractive -ExecutionPolicy Bypass -File <tmp.ps1> with cwd
# set to the version dir. Non-zero exit aborts the transaction.
#
# Files:
# - pre-install.ps1: expected preamble plus user script shape.
# - installer-args.txt: substitute() expectations for installer.args.
