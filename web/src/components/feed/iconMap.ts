import {
  FileText,
  BookOpen,
  Link as LinkIcon,
  Image as ImageIcon,
  TrendingUp,
  type LucideIcon,
} from "lucide-react";

export const ICON_MAP: Record<string, LucideIcon> = {
  note: FileText,
  paper: BookOpen,
  link: LinkIcon,
  image: ImageIcon,
  "stock-chart": TrendingUp,
};
