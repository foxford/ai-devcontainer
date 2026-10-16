import type { Vm } from './types';
import React, { useCallback } from 'react';
export function Total({ vm, factor }: { vm: Vm; factor: number }) {
  const multiply = useCallback((n: number) => n * factor, []);
  return <span>{vm.state.count.map(multiply).signal}</span>;
}
