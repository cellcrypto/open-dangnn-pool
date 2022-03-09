import Ember from 'ember';

export default Ember.Controller.extend({
  options: ['none', 'all', 'user'],
  selectedAccess: null,
  textID: "",
  textPass: "",
  textPassConfirm: "",
  textChangePass: "",
  textChangePassConfirm: "",
  errors: "",

  init() {
    this._super(...arguments);
    this.errors = [];
    this.textID = "";
    this.textPass = "";
    this.textPassConfirm = "";
    this.textChangePass = "";
    this.textChangePassConfirm = "";
    this.selectedAccess = this.options[0];
  },

  didUpdateAttrs() {
    this._super(...arguments);
    this.set('errors', []);
    this.errors = [];
    this.textID = "";
    this.textPass = "";
    this.textPassConfirm = "";
    this.textChangePass = "";
    this.textChangePassConfirm = "";
    this.selectedAccess = this.options[0];
  },

  actions: {
    required(event) {
      if (!event.target.value) {
        this.get('errors').pushObject({ message: `${event.target.name} is required`});
      } else {
        this.textID = event.target.value;
      }
    },
    requiredPass(event) {
      if (event.target.value) {
        this.textPass = event.target.value;
      }
    },
    requiredPassConfirm(event) {
      if (event.target.value) {
        this.textPassConfirm = event.target.value;
      }
    },
    requiredChangePass(event) {
      if (event.target.value) {
        this.textChangePass = event.target.value;
      }
    },
    requiredChangePassConfirm(event) {
      if (event.target.value) {
        this.textChangePassConfirm = event.target.value;
      }
    },
    create(event) {
      this.textChangePass = "";
      return this.textChangePass;
    },


  }
});
