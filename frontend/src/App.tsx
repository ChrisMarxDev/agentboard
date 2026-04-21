import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { DataProvider } from './hooks/DataContext'
import Layout from './components/shell/Layout'
import PageRenderer from './components/shell/PageRenderer'
import SkillsIndex from './components/skills/SkillsIndex'
import SkillDetail from './components/skills/SkillDetail'

export default function App() {
  return (
    <DataProvider>
      <BrowserRouter>
        <Layout>
          <Routes>
            <Route path="/skills" element={<SkillsIndex />} />
            <Route path="/skills/:slug" element={<SkillDetail />} />
            <Route path="*" element={<PageRenderer />} />
          </Routes>
        </Layout>
      </BrowserRouter>
    </DataProvider>
  )
}
