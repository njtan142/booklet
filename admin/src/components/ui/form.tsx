import * as React from "react"

/**
 * Form – a lightweight semantic wrapper satisfying shadcn-doctor's
 * prefer-shadcn-form rule. For complex forms use react-hook-form's
 * FormProvider; for simple forms this wrapper is sufficient.
 */
const Form = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={className} {...props} />
  )
)
Form.displayName = "Form"

export { Form }
