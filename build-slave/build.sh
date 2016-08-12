#!/bin/bash

target=$*
test -z "$target" && exit 1

BASE="dmerge-base"
id="dmerge-build-${RANDOM}"
machinectl clone "${BASE}" "${id}" || exit 1
systemd-nspawn -q -M "${id}" \
	       --bind /usr/portage --bind-ro /var/lib/layman \
	       --setenv="LANG=C" --setenv="LC_ALL=C" \
	       emerge -1btkq $target
result=$?
machinectl remove "${id}"
exit $result
