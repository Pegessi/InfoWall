import { Header } from "@/components/layout/Header";
import { FeedList } from "@/components/feed/FeedList";
import { useTheme } from "@/hooks/useTheme";

export default function App() {
  // Initialize theme (applies class + persists)
  useTheme();

  return (
    <>
      <Header />
      <main className="min-h-[calc(100vh-3rem)] w-full px-2 py-2 sm:px-3 lg:px-4">
        <FeedList />
      </main>
    </>
  );
}
