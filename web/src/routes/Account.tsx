import { Navigate } from 'react-router-dom'

/** /account is now unified into /settings — redirect transparently. */
export default function Account() {
  return <Navigate to="/settings" replace />
}
