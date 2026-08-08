import { BrowserRouter, Route, Routes } from 'react-router-dom';
import { ToastProvider } from './components/Toast';
import Dashboard from './pages/Dashboard';
import DesignPreview from './pages/DesignPreview';
import Reviews from './pages/Reviews';

export default function App() {
  return (
    <BrowserRouter>
      <ToastProvider>
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/reviews" element={<Reviews />} />
          <Route path="/design" element={<DesignPreview />} />
          <Route path="*" element={<Dashboard />} />
        </Routes>
      </ToastProvider>
    </BrowserRouter>
  );
}
