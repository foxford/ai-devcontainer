import React from 'react';
import type { Vm } from '../../after/client/types';
export function Child({ vm }: { vm: Vm }) {
  // Shared scenario VM: this screen does not own its lifecycle.
  return <span>{vm.state.count.signal}</span>;
}
