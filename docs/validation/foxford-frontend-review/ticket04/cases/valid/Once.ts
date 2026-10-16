import { ViewModel, createReactiveValue } from '@foxford/vm';
const state = createReactiveValue({ count: 2 });
export class Counter extends ViewModel<typeof state> {
  readonly double = this.state.count.map(n => n * 2);
}
export const ordinary = [1, 2].map(n => n * 2);
