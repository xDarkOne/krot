if systemctl is-active -q csqtt.service; then
  desc=$(systemctl show -p Description --value csqtt.service)
  [[ "$desc" == Krot* ]] || die "обнаружена служба csqtt вне управления Крота"
fi
