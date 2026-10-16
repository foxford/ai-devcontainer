import React from 'react';
import { Counter } from './catalog';
export function Item({ item, onSelect, onCleanup }) { return <Counter item={item} onSelect={onSelect} onCleanup={onCleanup} />; }
