import React, { useState, useEffect } from 'react';
export function Counter() {
  const [count, setCount] = useState(0);
  useEffect(() => { document.title = String(count); }, [count]);
  return <button onClick={() => setCount(count + 1)}>{count}</button>;
}
