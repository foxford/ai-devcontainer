import { ViewModel, createReactiveValue } from "@foxford/vm";
export function makeTyped() { return new ViewModel(createReactiveValue({ count: 2, busy: false })); }
export type Vm = ReturnType<typeof makeTyped>;
export type ItemData = { id: string; name: string };
export type ItemProps = { item: ItemData; onSelect: (id: string) => void };
export type ScreenProps = { vm: Vm; factor: number; items: ItemData[]; onSelect: (id: string) => void };
