if systemctl is-active -q aivpn-server.service; then
  desc=$(systemctl show -p Description --value aivpn-server.service)
  [[ "$desc" == Krot* ]] || die "обнаружена служба aivpn-server вне управления Крота"
fi
