# relocate: откат прогона.
restore_snapshot
for l in /usr/local/sbin/relocate-*; do
  [ -L "$l" ] && grep -qxF "$l" "$VK_RUN/created.list" 2>/dev/null && rm -f "$l"
done
say "откат relocate выполнен"
