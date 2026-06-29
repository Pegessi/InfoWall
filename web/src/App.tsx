import { Header } from "@/components/layout/Header";
import { FeedList } from "@/components/feed/FeedList";
import { useTheme } from "@/hooks/useTheme";

export default function App() {
  // Initialize theme (applies class + persists)
  useTheme();

  return (
    <>
      <Header />
      <main className="mx-auto w-full max-w-[1600px] px-4 py-6 sm:px-6 sm:py-8">
        <FeedList />
      </main>
    </>
  );
}
