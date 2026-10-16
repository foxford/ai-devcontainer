const { ViewModel, createReactiveValue } = require('@foxford/vm');
const double = n => n * 2;
exports.make = onDestroy => new ViewModel(createReactiveValue({ count: 2, visible: true, busy: false }), { onDestroy });
exports.setDoubled = (vm, next) => vm.state.count.set(next / 2);
exports.finish = vm => vm.destroy();
