import { builtinEnvironments } from 'vitest/environments';

const environment: typeof builtinEnvironments.jsdom = {
  ...builtinEnvironments.jsdom,
  async setup(global, options) {
    const storageNames = ['localStorage', 'sessionStorage'];
    const originals = storageNames.map(name => Object.getOwnPropertyDescriptor(global, name));
    const restore = () => storageNames.forEach((name, index) => {
      delete global[name];
      if (originals[index]) Object.defineProperty(global, name, originals[index]!);
    });
    // Vitest 0.34 keeps pre-existing Node globals instead of jsdom's browser storage.
    storageNames.forEach(name => { delete global[name]; });
    try {
      const jsdom = await builtinEnvironments.jsdom.setup(global, options);
      return {
        async teardown(global) {
          try {
            await jsdom.teardown(global);
          } finally {
            restore();
          }
        },
      };
    } catch (error) {
      restore();
      throw error;
    }
  },
};

export default environment;
