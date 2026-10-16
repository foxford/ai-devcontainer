import React, { useEffect, useState } from 'react';
import { prefix } from '../model';
export function Counter({ item, onSelect, onCleanup }) {
  const [count, setCount] = useState(0);
  useEffect(() => () => onCleanup(item.id), [item.id, onCleanup]);
  return <li data-id={prefix + item.id}><input defaultValue={item.name} /><button onClick={() => { setCount(n => n + 1); onSelect(item.id); }}>{item.name}:{count}</button></li>;
}
