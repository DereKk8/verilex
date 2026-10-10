#!/bin/bash
# Proves the dedicated-user workaround: run the launcher as user $1 (or as the current user when
# $1 is empty), let the brain kill sandbox-init and leave a setsid child, then list and end what is left.
set -euo pipefail
user=${1:-}
w=$(mktemp -d /tmp/vxproof.XXXXXX); chmod 755 "$w"
go build -o "$w/verilex-agent" ./agent/cmd/verilex-agent
go build -o "$w/verilex" ./cmd/verilex
cp -R tests/fixtures/tally "$w/project"
git -C "$w/project" init -q && git -C "$w/project" add -A && git -C "$w/project" -c user.name=v -c user.email=v@example.com commit -qm base
printf 'skill text\n' > "$w/skill.md"
cat > "$w/brain" <<'B'
#!/bin/sh
python3 -c 'import os, time; os.setsid(); time.sleep(120)' vx-leftover-child &
sleep 1
kill -9 $PPID
sleep 60
B
chmod -R a+rX "$w"; chmod +x "$w/brain"
as=()
if [ -n "$user" ]; then
  mkdir -p "$w/home" "$w/tmp"; sudo chown -R "$user" "$w/project" "$w/home" "$w/tmp"
  as=(sudo -u "$user" env HOME="$w/home" TMPDIR="$w/tmp" PATH="$PATH")
fi
set +e
time "${as[@]}" "$w/verilex-agent" --verilex "$w/verilex" --skill "$w/skill.md" --project "$w/project" --brain "$w/brain" \
  --intent 'prove the store opens' --claim store-opened --harness stub --model stub
echo "launcher exit $?"
set -e
sleep 1
echo "== leftovers right after the run"
if [ -n "$user" ]; then pgrep -l -u "$user" || echo "none"; else pgrep -lf '[v]x-leftover-child' || echo "none"; fi
if [ -n "$user" ]; then
  echo "== sudo pkill -KILL -u $user"
  sudo pkill -KILL -u "$user" || true
  sleep 1
  echo "== leftovers after pkill"
  pgrep -l -u "$user" || echo "none"
fi
