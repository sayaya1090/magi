#!/bin/sh
# Office 를 켤 때 헬퍼를 띄운다 — mac 판.
#
# Windows 에서는 COM 추가 기능이 Office 프로세스 **안에서** 뜨며 헬퍼를 띄운다(clients/office/addin-com). Office for Mac
# 에는 그 자리가 없다 — 맥은 웹 애드인과 VBA 만 받고 COM 추가 기능을 안 받는다. .NET 이 맥에 있고 없고의 문제가 아니라
# (있다) **Office 가 남의 코드를 자기 프로세스에 들일 길이 맥에는 없다.**
#
# 그래서 맥에서는 launchd 가 그 자리를 대신한다. `StartInterval` 로 이 스크립트를 10초마다 한 번 돌리는데, **도는 사이에는
# 아무것도 안 떠 있다** — 쉬는 프로세스를 두지 않는 것이 요점이다(사용자, 2026-09-07: 「쓰지도 않는데 켜져 있는 건 악성코드
# 아니냐」). Office 가 하나라도 떠 있고 헬퍼가 없으면 띄우고, 그것 말고는 아무것도 안 한다. Office 를 다 끄면 헬퍼가 스스로
# 끝난다(helper/idle.go officeWatch).
#
#   ./office-start.sh --install /path/to/magi   # launchd 에 건다(한 번)
#   ./office-start.sh --uninstall               # 뗀다
#   ./office-start.sh                           # 한 번 본다(launchd 가 이렇게 부른다)
set -eu

LABEL=dev.magi.office.start
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
PORT=3000
SELF=$(cd "$(dirname "$0")" && pwd)/$(basename "$0")

helper_running() {
  # 포트가 열려 있으면 떠 있는 것이다. 이름으로 세면 평소 magi 데몬까지 센다.
  nc -z 127.0.0.1 "$PORT" >/dev/null 2>&1
}

office_running() {
  pgrep -x "Microsoft Excel" >/dev/null 2>&1 ||
    pgrep -x "Microsoft Word" >/dev/null 2>&1 ||
    pgrep -x "Microsoft PowerPoint" >/dev/null 2>&1
}

case "${1:-}" in
  --install)
    magi=${2:-}
    [ -n "$magi" ] || { echo "쓰는 법: $0 --install /path/to/magi" >&2; exit 2; }
    [ -x "$magi" ] || { echo "그 자리에 실행 파일이 없습니다: $magi" >&2; exit 2; }
    mkdir -p "$HOME/Library/LaunchAgents"
    cat > "$PLIST" <<PLIST_END
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key>
  <array>
    <string>/bin/sh</string>
    <string>$SELF</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict><key>MAGI_OFFICE_HELPER</key><string>$magi</string></dict>
  <key>StartInterval</key><integer>10</integer>
  <key>RunAtLoad</key><false/>
</dict>
</plist>
PLIST_END
    launchctl unload "$PLIST" 2>/dev/null || true
    launchctl load "$PLIST"
    echo "걸었습니다: $PLIST"
    echo "  Office 를 켜면 10초 안에 헬퍼가 뜨고, 다 끄면 헬퍼가 스스로 끝납니다."
    ;;
  --uninstall)
    launchctl unload "$PLIST" 2>/dev/null || true
    rm -f "$PLIST"
    echo "뗐습니다: $PLIST"
    ;;
  "")
    magi=${MAGI_OFFICE_HELPER:-magi}
    office_running || exit 0
    helper_running && exit 0
    # 부모(launchd 의 이 한 번)가 끝나도 살아 있게 떼어 놓는다.
    nohup "$magi" office >/dev/null 2>&1 &
    ;;
  *)
    echo "쓰는 법: $0 [--install /path/to/magi | --uninstall]" >&2
    exit 2
    ;;
esac
