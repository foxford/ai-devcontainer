import type { Vm } from './types';
import React from 'react';
export function Controls({ vm }: { vm: Vm }) {
  const visible = vm.state.visible.use();
  const busy = vm.state.busy.use();
  return visible ? <button disabled={busy}>Run</button> : null;
}
