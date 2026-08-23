import js from "@eslint/js";
import tseslint from "typescript-eslint";
import reactHooks from "eslint-plugin-react-hooks";
import jsxA11y from "eslint-plugin-jsx-a11y";

// The package advertised a `lint` script for two years with no eslint installed and no
// config, so it could only ever fail. This is that script made real.
//
// The rule set is deliberately narrow. A linter turned up to eleven on an existing
// codebase produces hundreds of findings nobody triages, and the whole thing gets
// switched off — so this enables the rules that catch the classes of bug the review
// actually found, and leaves stylistic opinions to the reader.
export default tseslint.config(
  { ignores: ["dist/**", "node_modules/**", "src/api/gen/**"] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["src/**/*.{ts,tsx}"],
    plugins: { "react-hooks": reactHooks, "jsx-a11y": jsxA11y },
    languageOptions: {
      parserOptions: { ecmaFeatures: { jsx: true } },
      globals: { window: "readonly", document: "readonly", localStorage: "readonly", console: "readonly" },
    },
    rules: {
      ...reactHooks.configs.recommended.rules,

      // The Board's auto-pager re-fired on failure because isFetchingNextPage flipping
      // back to false is itself a dependency change. That is exactly this rule's
      // territory, and it was the most expensive frontend bug in the review — an
      // unbounded request loop against the API from a single failed page.
      "react-hooks/exhaustive-deps": "warn",

      // Accessibility rules matching the defects that were actually present: modals
      // with no role or label, and interactive elements reachable only by mouse.
      "jsx-a11y/role-has-required-aria-props": "error",
      "jsx-a11y/aria-props": "error",
      "jsx-a11y/aria-role": "error",
      "jsx-a11y/no-redundant-roles": "error",
      "jsx-a11y/click-events-have-key-events": "warn",
      "jsx-a11y/no-static-element-interactions": "warn",
      "jsx-a11y/anchor-is-valid": "warn",

      // An unused variable is usually a half-finished edit; an unused *argument* often
      // is not, so the underscore convention opts out.
      "@typescript-eslint/no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_", varsIgnorePattern: "^_" },
      ],

      // `any` erases exactly the checking the generated client exists to provide.
      // A warning rather than an error: there are a few already, and failing the build
      // on them would mean turning the rule off instead of fixing them.
      "@typescript-eslint/no-explicit-any": "warn",
    },
  },
);
