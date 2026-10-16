import React from "react";
import { useIntl } from "react-intl";
import { prefix } from "./catalog/constants";
import { Button } from "./ui/Button";
// USER: unfinished personal note
function Empty() { return <p>Empty</p>; }
export function Screen({ vm, factor, items, onSelect }) {
  const intl = useIntl();
  const renderItem = item => <li key={item.id} data-id={prefix + item.id}><button onClick={() => onSelect(item.id)}>{item.name}</button></li>;
  const total = vm.state.count.map(n => n * factor);
  const busy = vm.state.busy.signal;
  return <section className="panel"><h2>{intl.formatMessage({ id: "heading", defaultMessage: "Hello {name}" }, { name: "Ada" })}</h2>
    <input placeholder={intl.formatMessage({ id: "search", defaultMessage: "Search" })} />
    <output>{total.use()}</output><Button disabled={busy}>Save</Button>
    <ul>{items.map(item => renderItem(item))}</ul>{items.length ? null : <Empty />}
    <article className="articleBody"><p>Editor content</p><Button><span>Embedded</span></Button></article>
  </section>;
}
