import { AuthProvider } from './contexts/AuthContext'
import { Home } from './pages/Home'
import './App.css'

function AppRouter() {
  // Temporarily ignore users/onboarding and always show the main page
  return <Home />
}

function App() {
  return (
    <AuthProvider>
      <AppRouter />
    </AuthProvider>
  )
}

export default App
