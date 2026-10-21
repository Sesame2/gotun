import { CssBaseline } from '@mui/material';
import { BrowserRouter } from 'react-router-dom';
import Layout from './components/Layout';
import { ThemeProvider, I18nProvider } from './providers';
import './styles/global.css';

function App() {
  return (
    <I18nProvider>
      <ThemeProvider>
        <CssBaseline />
        <BrowserRouter>
          <Layout />
        </BrowserRouter>
      </ThemeProvider>
    </I18nProvider>
  );
}

export default App
