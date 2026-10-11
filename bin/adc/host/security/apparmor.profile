# SPDX-FileCopyrightText: Copyright The Moby Authors
# SPDX-License-Identifier: Apache-2.0
# Based on moby/profiles 6fe7deb1b9fb7c0397a4593480d7d22b9ee8caef.
# ADC permits nested mounts/pivot_root; kernel capabilities still apply.
abi <abi/3.0>,
#include <tunables/global>
profile "ADC_PROFILE_NAME" flags=(attach_disconnected,mediate_deleted) {
  #include <abstractions/base>
  network,
  deny network alg,
  deny network vsock,
  capability,
  file,
  umount,
  signal (receive) peer=unconfined,
  signal (receive) peer=runc,
  signal (receive) peer=crun,
  signal (receive) peer="/usr/bin/dockerd",
  signal (receive) peer="/usr/bin/docker",
  signal (send,receive) peer="ADC_PROFILE_NAME",
  deny @{PROC}/* w,
  deny @{PROC}/{[^1-9/],[^1-9/][^0-9/],[^1-9s/][^0-9y/][^0-9s/],[^1-9/][^0-9/][^0-9/][^0-9/]*}/** w,
  deny @{PROC}/sys/[^k]** w,
  deny @{PROC}/sys/kernel/{?,??,[^s][^h][^m]**} w,
  deny @{PROC}/sysrq-trigger rwklx,
  deny @{PROC}/kcore rwklx,
  mount,
  pivot_root,
  deny /sys/[^f]*/** wklx,
  deny /sys/f[^s]*/** wklx,
  deny /sys/fs/[^c]*/** wklx,
  deny /sys/fs/c[^g]*/** wklx,
  deny /sys/fs/cg[^r]*/** wklx,
  deny /sys/firmware/** rwklx,
  deny /sys/devices/virtual/powercap/** rwklx,
  deny /sys/kernel/security/** rwklx,
  ptrace (trace,tracedby,read,readby) peer="ADC_PROFILE_NAME",
}
