import React, { useCallback } from "react";
import { FormattedMessage, useIntl } from "react-intl";
import { Item } from "./Item";
import { Empty } from "./Empty";
import { Button } from "./ui/Button";
// USER: unfinished personal note
import type { ScreenProps } from "./types";
export function Screen({ vm, factor, items, onSelect }: ScreenProps) {
  const intl = useIntl();
  const multiply = useCallback((n: number) => n * factor, [factor]);
  const total = vm.state.count.map(multiply);
  const busy = vm.state.busy.use();
  return <section className="panel"><h2 className="heading"><FormattedMessage id="heading" defaultMessage="Hello {name}" values={{ name: "Ada" }} /></h2>
    <input placeholder={intl.formatMessage({ id: "search", defaultMessage: "Search" })} />
    <output>{total.signal}</output><Button disabled={busy}>Save</Button>
    <ul>{items.map(item => <Item key={item.id} item={item} onSelect={onSelect} />)}</ul>{items.length ? null : <Empty />}
    <article className="articleBody"><p>Editor content</p><Button><span>Embedded</span></Button></article>
  </section>;
}
