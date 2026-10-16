import React from 'react';
import type { Vm } from './types';
const double = (n: number) => n * 2;
export function Pure({ vm }: { vm: Vm }) {
  return <span>{vm.state.count.map(double).signal}</span>;
}
