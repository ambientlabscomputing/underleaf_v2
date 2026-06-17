import { useState } from 'react';
import { Home } from './pages';
import { Containers } from './pages';

type Page = 'nodes' | 'containers';

function App() {
  const [page, setPage] = useState<Page>('nodes');

  if (page === 'containers') {
    return <Containers onNavigate={setPage} />;
  }

  return <Home onNavigate={setPage} />;
}

export default App;

