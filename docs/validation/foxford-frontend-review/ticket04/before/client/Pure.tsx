import React from 'react';
import type { Vm } from './types';
export function Pure({ vm }: { vm: Vm }) {
  return <span>{vm.state.count.map(n => n * 2).signal}</span>;
}
