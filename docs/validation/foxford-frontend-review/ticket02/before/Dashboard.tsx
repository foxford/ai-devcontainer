import React from 'react';
import { useIntl } from 'react-intl';
import { Counter } from './catalog';
import { prefix } from './catalog/model';
import type { Product } from './catalog/model';
const Empty = () => <p>No items</p>;
export function Dashboard({ items, onSelect, onCleanup }: { items: Product[]; onSelect: (id: string) => void; onCleanup: (id: string) => void }) {
  const intl = useIntl();
  const renderItem = (item: Product) => <Counter key={item.id} item={item} onSelect={onSelect} onCleanup={onCleanup} />;
  return <section data-prefix={prefix}><h1>{intl.formatMessage({ id: 'greeting', defaultMessage: 'Hello {name}' }, { name: 'Ada' })}</h1><input placeholder={intl.formatMessage({ id: 'search', defaultMessage: 'Search' })} />{items.length ? <ul>{items.map(item => renderItem(item))}</ul> : <Empty />}</section>;
}
