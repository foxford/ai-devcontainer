import React from "react";
export function Button({ children, disabled, onClick, size = "medium" }) { return <button className="kitButton" data-size={size} disabled={disabled} onClick={onClick}>{children}</button>; }
