import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { DataProvider } from './hooks/DataContext'
import Layout from './components/shell/Layout'
import PageRenderer from './components/shell/PageRenderer'
import FileViewer from './components/files/FileViewer'

export default function App() {
  return (
    <DataProvider>
      <BrowserRouter>
        <Layout>
          <Routes>
            <Route path="/files/*" element={<FileViewer />} />
            <Route path="*" element={<PageRenderer />} />
          </Routes>
        </Layout>
      </BrowserRouter>
    </DataProvider>
  )
}
