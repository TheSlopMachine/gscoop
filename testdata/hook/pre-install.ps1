$dir = $env:GSCOOP_HOOK_DIR
$original_dir = $env:GSCOOP_HOOK_ORIGINAL_DIR
$persist_dir = $env:GSCOOP_HOOK_PERSIST_DIR
$version = $env:GSCOOP_HOOK_VERSION
$architecture = $env:GSCOOP_HOOK_ARCHITECTURE
$global = $false
$SCOOP = $env:GSCOOP_HOOK_SCOOP
$SCOOP_GLOBAL = $env:GSCOOP_HOOK_SCOOP_GLOBAL
Write-Host "pre_install $version $architecture"
