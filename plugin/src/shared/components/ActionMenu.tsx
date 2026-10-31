import { useId, useState, type ButtonHTMLAttributes, type KeyboardEvent, type MouseEvent } from "react";
import { createPortal } from "react-dom";
import { IconButton } from "./IconButton";
import { MenuSurface } from "./MenuSurface";

export interface ActionMenuItem {
  id: string;
  label: string;
  onSelect: (anchor: HTMLButtonElement) => void;
  disabled?: boolean;
  checked?: boolean;
  destructive?: boolean;
  separator?: boolean;
}
export interface ActionMenuProps {
  label: string;
  menuLabel: string;
  items: ActionMenuItem[];
  disabled?: boolean;
  triggerProps?: ButtonHTMLAttributes<HTMLButtonElement> & Record<`data-${string}`, string>;
  text?: string;
}
export function ActionMenu({ label, menuLabel, items, disabled, triggerProps, text }: ActionMenuProps) {
  const id = useId();
  const [open, setOpen] = useState<{ anchor: HTMLButtonElement; edge: "first" | "last" } | null>(null);
  const close = (restoreFocus: boolean) => {
    if (restoreFocus) open?.anchor.focus({ preventScroll: true });
    setOpen(null);
  };
  const keyboard = (event: KeyboardEvent<HTMLButtonElement>) => {
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
    event.preventDefault();
    setOpen({ anchor: event.currentTarget, edge: event.key === "ArrowUp" ? "last" : "first" });
  };
  const trigger = { ...triggerProps, disabled, "aria-haspopup": "menu" as const, "aria-expanded": Boolean(open), "aria-controls": open ? id : undefined, onKeyDown: keyboard, onClick: (event: MouseEvent<HTMLButtonElement>) => setOpen(open ? null : { anchor: event.currentTarget, edge: "first" as const }) };
  return <>{text ? <button {...trigger} type="button" className="topic-stream-refresh" aria-label={label}>{text}</button> : <IconButton {...trigger} icon="more" label={label} />}
    {open && createPortal(<MenuSurface id={id} label={menuLabel} items={items} anchor={open.anchor} edge={open.edge} onClose={close} />, open.anchor.ownerDocument.body)}
  </>;
}
