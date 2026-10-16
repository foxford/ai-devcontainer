import React from 'react';
import { FormattedMessage, useIntl } from 'react-intl';
import { prefix } from './catalog';
import type { Product } from './catalog';
import { Empty } from './Empty';
import { Item } from './Item';
export function Dashboard({ items, onSelect, onCleanup }: { items: Product[]; onSelect: (id: string) => void; onCleanup: (id: string) => void }) {
  const intl = useIntl();
  return <section data-prefix={prefix}><h1><FormattedMessage id="greeting" defaultMessage="Hello {name}" values={{ name: 'Ada' }} /></h1><input placeholder={intl.formatMessage({ id: 'search', defaultMessage: 'Search' })} />{items.length ? <ul>{items.map(item => <Item key={item.id} item={item} onSelect={onSelect} onCleanup={onCleanup} />)}</ul> : <Empty />}</section>;
}
