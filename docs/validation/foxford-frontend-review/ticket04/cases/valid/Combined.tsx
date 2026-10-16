import React from 'react';
import type { Vm } from '../../after/client/types';
export function Combined({ vm }: { vm: Vm }) {
  const count = vm.state.count.use();
  return <button disabled={count === 0} onClick={() => vm.state.count.set(count + 1)}>{count}</button>;
}
