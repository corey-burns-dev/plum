import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import { forwardRef } from "react";
import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-[var(--radius-md)] text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--plum-ring)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--plum-bg)] disabled:pointer-events-none disabled:opacity-50 [&_svg]:size-4 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        default: "bg-[var(--plum-accent)] text-white shadow-sm hover:bg-[var(--plum-accent)]/90",
        secondary:
          "border border-[var(--plum-border)] bg-[var(--plum-panel)] text-[var(--plum-text)] hover:bg-[var(--plum-panel-alt)] hover:border-[var(--plum-accent-soft)]",
        ghost:
          "text-[var(--plum-muted)] hover:bg-[var(--plum-panel)] hover:text-[var(--plum-text)]",
        link: "text-[var(--plum-accent)] underline-offset-4 hover:underline",
        icon: "text-[var(--plum-muted)] hover:bg-[var(--plum-panel)] hover:text-[var(--plum-text)]",
      },
      size: {
        default: "h-9 px-4 py-2",
        sm: "h-8 rounded-[var(--radius-sm)] px-3 text-xs",
        lg: "h-10 rounded-[var(--radius-lg)] px-6",
        icon: "h-9 w-9",
        "icon-sm": "h-8 w-8",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  },
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : "button";
    return (
      <Comp className={cn(buttonVariants({ variant, size, className }))} ref={ref} {...props} />
    );
  },
);
Button.displayName = "Button";

export { Button, buttonVariants };
