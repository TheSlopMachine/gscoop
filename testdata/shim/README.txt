# Shim triple fixture notes
#
# Triple for name demo:
# - demo.exe: byte copy of the selected shim payload (kiennq default;
#   71 on SHIM=71; scoopcs maps to kiennq). GUI targets patch the copy
#   to subsystem 2. x86 shim on x64 rewrites System32 to Sysnative
#   and SysWOW64 to System32 in the recorded path.
# - demo.shim: path = "<resolved>" plus optional args = <arg> lines.
# - demo.cmd plus extensionless wrapper per target type (.bat, .ps1,
#   .jar, .py, generic). Alias triples repeat as scoop-<alias>.*.
# - Overwrite moves foreign shims to <shim>.<owner>; removal restores
#   the newest backup and drops demo.exe only with no backups left.
#
# Files:
# - demo.shim: expected .shim text shape.
# - wrappers.txt: per-type wrapper expectations.
