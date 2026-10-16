import type { Vm } from '../../after/client/types';
const clamp = (n: number) => Math.min(n, 10);
export function write(vm: Vm) {
  const capped = vm.state.count.map(clamp);
  capped.set(10); // Cannot infer whether source must be 10, 20 or another value.
}
