# Static Assets Testing

This directory contains static assets for the prcost web UI, including JavaScript functions that are tested separately.

## JavaScript Testing

Key functions are extracted into separate `.js` files for testing purposes:

- `formatR2RCallout.js` - Renders the Ready to Review savings callout
- `formatR2RCallout.test.js` - Tests for the callout rendering

### Running Tests

```bash
# Run JavaScript tests only
make test-js

# Run all tests (Go + JavaScript)
make test
```

### Test Coverage

The JavaScript tests verify:
- Correct rendering of the savings callout HTML
- Proper formatting of dollar amounts ($50K, $2.5M, etc.)
- Presence of key messaging ("Pro-Tip:", "Ready to Review", etc.)
- Correct behavior for fast PRs (no callout for ≤1 hour)
- HTML structure and styling

### Adding New Tests

When modifying `index.html` JavaScript functions:

1. Extract the function to a separate `.js` file (if not already extracted)
2. Add tests to the corresponding `.test.js` file
3. Run `make test-js` to verify
4. Commit both the function and test files together
