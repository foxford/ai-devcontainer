import type { Vm } from './types';
import React from 'react';
export function Text({ vm }: { vm: Vm }) {
  const text = vm.state.count.use();
  return <span>{text}</span>;
}
