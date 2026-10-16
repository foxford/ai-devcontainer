// Review fixture fragments; identifiers are supplied by the host project.
// render props and callbacks: valid
<DataProvider>{data => <span>{data.name}</span>}</DataProvider>;
<VirtualList renderItem={item => <Row item={item} />} />;
items.map(item => <li key={item.id}>{item.name}</li>);
<button onClick={() => save(id)}>Save</button>;
// styled declarations: valid with one production component
const Root = styled.div``;
const Title = styled.h2``;
export function Card() { return <Root><Title>Title</Title></Root>; }
// string-only: valid
<input placeholder={intl.formatMessage(messages.search)} aria-label={intl.formatMessage(messages.search)} />;
<img alt={intl.formatMessage(messages.cover)} />;
document.title = intl.formatMessage(messages.title);
// other i18n stack: FormattedMessage absent, do not migrate
<h1>{t('title')}</h1>;
// ambiguous helper: proposal only; moving hook changes ownership
function Stateful() {
  const renderBody = () => { const [n] = useState(0); return <p>{n}</p>; };
  return renderBody();
}
