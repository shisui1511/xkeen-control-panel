// Simple PWA icon generator - run with Node.js to create PNG icons
// Requires: npm install sharp
// Usage: node generate-icons.js

// Simplified - use a canvas-based approach
const sizes = [72, 96, 128, 144, 152, 192, 384, 512];
console.log('Icon generator requires sharp package: npm install sharp');
console.log('Or use online generator: https://favicon.io/icon-generator');
sizes.forEach((size) => {
  console.log(`Need icon: icons/icon-${size}x${size}.png`);
});
