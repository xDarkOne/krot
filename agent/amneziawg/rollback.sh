# amneziawg: откат прогона. Пакеты сборки не удаляются (безвредны).
: "${VK_TAG:=v3.1.20260906}"
want=${VK_TAG#v}
ip link del vk-awgtest 2>/dev/null || true
if grep -qxF "dkms amneziawg/$want" "$VK_RUN/created.list" 2>/dev/null; then
  # модуль выгружаем, только если им никто не пользуется (нет интерфейсов amneziawg)
  if ! grep -q . <<<"$(ip -o link show type amneziawg 2>/dev/null)"; then rmmod amneziawg 2>/dev/null || true; fi
  dkms remove -m amneziawg -v "$want" --all >/dev/null 2>&1 || true
fi
restore_snapshot
systemctl daemon-reload
for d in /lib/modules/*/modules.dep; do k=${d%/modules.dep}; depmod -a "${k##*/}" 2>/dev/null || true; done
say "откат amneziawg выполнен"
