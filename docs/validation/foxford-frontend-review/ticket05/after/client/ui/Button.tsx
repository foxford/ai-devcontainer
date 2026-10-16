import React from "react";
type Props = { children: React.ReactNode; disabled?: boolean; onClick?: () => void; size?: "small" | "medium" };
export function Button({ children, disabled, onClick, size = "medium" }: Props) { return <button className="kitButton" data-size={size} disabled={disabled} onClick={onClick}>{children}</button>; }
