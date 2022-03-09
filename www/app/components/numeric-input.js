import Ember from 'ember';

export default Ember.TextField.extend({
  tagName: "input",
  type: "number",
  attributeBindings: [ "name", "type", "value", "maxlength", "id" ],
  keyDown: function(e) {
    var key = e.charCode || e.keyCode || 0;
    // allow enter, backspace, tab, delete, numbers, keypad numbers, home, end only.
    return (
      key === 13 ||
      key === 8 ||
      key === 9 ||
      key === 46 ||
      (key >= 35 && key <= 40) ||
      (key >= 48 && key <= 57) ||
      (key >= 96 && key <= 105));
  },
  keyPress: function() {
    var inputValue = this.value || "";
    return (inputValue.length < this.maxlength);
  }
});
