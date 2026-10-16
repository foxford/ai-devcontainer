import { ViewModel, createReactiveValue } from '@foxford/vm';
export function makeVm() {
  return new ViewModel(createReactiveValue({ count: 2, visible: true, busy: false }));
}
export type Vm = ReturnType<typeof makeVm>;
