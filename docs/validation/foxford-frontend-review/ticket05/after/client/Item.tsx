import React from "react";
import { prefix } from "./catalog";
import type { ItemProps } from "./types";
export function Item({ item, onSelect }: ItemProps) { return <li data-id={prefix + item.id}><button onClick={() => onSelect(item.id)}>{item.name}</button></li>; }
