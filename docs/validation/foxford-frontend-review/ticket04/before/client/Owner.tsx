import type { Vm } from './types';
import React from 'react';
export function Owner({ vm }: { vm: Vm }) {
  vm.useAutoDestroy();
  return <button onClick={() => vm.enableAutoDestroy()}>Finish</button>;
}
