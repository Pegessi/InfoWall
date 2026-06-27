import { Header } from "@/components/layout/Header";
import { FeedList } from "@/components/feed/FeedList";
import { useTheme } from "@/hooks/useTheme";

export default function App() {
  // Initialize theme (applies class + persists)
  useTheme();

  return (
    <>
      <Header />
      <main className="mx-auto max-w-2xl px-4 py-8 sm:py-10">
        <FeedList />
      </main>
    </>
  );
}
