#!/bin/sh
# Dry-run detection with mocked uname/os-release; no Linux binary is executed.
set -eu
ROOT=$1
TEST_DIR=$2
mkdir -p "$TEST_DIR/mockbin"
cat > "$TEST_DIR/mockbin/uname" <<'EOF'
#!/bin/sh
case "$1" in -m) echo "$FAKE_CPU";; -r) echo "${FAKE_KERNEL:-6.6.0}";; -s) echo Linux;; esac
EOF
chmod +x "$TEST_DIR/mockbin/uname"
PATH="$TEST_DIR/mockbin:$PATH"
PDF_APP_ROOT=$ROOT
PDF_OS_RELEASE_FILE="$TEST_DIR/os-release"
export PATH PDF_APP_ROOT PDF_OS_RELEASE_FILE
for name in 'openEuler 24.03 LTS SP2' 'Kylin Linux Advanced Server V10' 'UnionTech OS Server 20'; do
  printf 'PRETTY_NAME="%s"\n' "$name" > "$PDF_OS_RELEASE_FILE"
  for cpu in x86_64 aarch64 loongarch64; do
    FAKE_CPU=$cpu; export FAKE_CPU
    output=$(sh "$ROOT/html/start-linux.sh" --check)
    printf '%s\n' "$output" | grep -F "$name / $cpu" >/dev/null
    printf 'PASS: dry-run %s / %s\n' "$name" "$cpu"
  done
done
printf 'PRETTY_NAME="Alibaba Cloud Linux 3.2104 LTS"\n' > "$PDF_OS_RELEASE_FILE"
FAKE_KERNEL=5.10.134-19.2.al8.x86_64
export FAKE_KERNEL
for cpu in x86_64 aarch64; do
  FAKE_CPU=$cpu; export FAKE_CPU
  output=$(sh "$ROOT/html/start-linux.sh" --check)
  printf '%s\n' "$output" | grep -F "Alibaba Cloud Linux 3.2104 LTS / $cpu" >/dev/null
  printf 'PASS: dry-run Alibaba Cloud Linux 3 / %s / kernel 5.10\n' "$cpu"
done
unset FAKE_KERNEL
FAKE_CPU=mips64; export FAKE_CPU
if sh "$ROOT/html/start-linux.sh" --check >/dev/null 2>&1; then echo 'FAIL: accepted unsupported MIPS'; exit 1; fi
FAKE_CPU=loongarch64; FAKE_KERNEL=5.10.0; export FAKE_CPU FAKE_KERNEL
if sh "$ROOT/html/start-linux.sh" --check >/dev/null 2>&1; then echo 'FAIL: accepted old LoongArch kernel'; exit 1; fi
echo 'PASS: unsupported CPU and old LoongArch kernel refused'
