const { ViewModel, createReactiveValue } = require("@foxford/vm");
exports.make = onDestroy => new ViewModel(createReactiveValue({ count: 2, busy: false }), { onDestroy });
exports.setDoubled = (vm, next) => vm.state.count.set(next / 2);
exports.finish = vm => vm.destroy();
