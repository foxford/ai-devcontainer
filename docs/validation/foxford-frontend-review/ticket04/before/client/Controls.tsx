import type { Vm } from './types';
import React from 'react';
export function Controls({ vm }: { vm: Vm }) {
  return vm.state.visible.signal ? <button disabled={vm.state.busy.signal}>Run</button> : null;
}
