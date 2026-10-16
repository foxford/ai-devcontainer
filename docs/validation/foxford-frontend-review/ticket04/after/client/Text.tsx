import type { Vm } from './types';
import React from 'react';
export function Text({ vm }: { vm: Vm }) {
  return <span>{vm.state.count.signal}</span>;
}
