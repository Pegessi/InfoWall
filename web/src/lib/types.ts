export interface Item {
  id: string;
  type: string;
  title: string;
  tags: string[];
  pinned: boolean;
  meta: Record<string, any>;
  body: string;
  created_at: string;
}
