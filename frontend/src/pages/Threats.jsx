import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import Footer from '../components/Footer'
import PageHeader from '../components/PageHeader'
import ThreatsList from '../components/ThreatsList'

export default function Threats() {
  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />
      <Footer />
      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        <div className="p-8">
          <PageHeader
            title="Threats"
            subtitle="Deduplicated security threats detected across AI agents."
          />
          <ThreatsList showFilters />
        </div>
      </main>
    </div>
  )
}
