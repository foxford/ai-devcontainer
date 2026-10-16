const { ViewModel, createReactiveValue } = require("@foxford/vm");
exports.make = onDestroy => new ViewModel(createReactiveValue({ count: 2, busy: false }), { onDestroy });
exports.setDoubled = (vm, next) => vm.state.count.map(n => n * 2).set(next);
exports.finish = vm => vm.state.destroy();
