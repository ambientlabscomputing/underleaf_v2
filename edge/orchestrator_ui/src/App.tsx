import { Navigate, Route, Routes } from 'react-router-dom';
import { Home, Containers, ContainerLogs, Deployments } from './pages';

function App() {
  return (
    <Routes>
      <Route path="/nodes" element={<Home />} />
      <Route path="/containers" element={<Containers />} />
      <Route path="/containers/:dockerId/logs" element={<ContainerLogs />} />
      <Route path="/deployments" element={<Deployments />} />
      <Route path="*" element={<Navigate to="/nodes" replace />} />
    </Routes>
  );
}

export default App;

